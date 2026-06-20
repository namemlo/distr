import {HttpClient} from '@angular/common/http';
import {inject, Injectable} from '@angular/core';
import {Observable} from 'rxjs';
import {Channel, CreateUpdateChannelRequest} from '../types/channel';

const baseUrl = '/api/v1/channels';

@Injectable({
  providedIn: 'root',
})
export class ChannelsService {
  private readonly httpClient = inject(HttpClient);

  list(): Observable<Channel[]> {
    return this.httpClient.get<Channel[]>(baseUrl);
  }

  get(id: string): Observable<Channel> {
    return this.httpClient.get<Channel>(`${baseUrl}/${id}`);
  }

  create(request: CreateUpdateChannelRequest): Observable<Channel> {
    return this.httpClient.post<Channel>(baseUrl, request);
  }

  update(id: string, request: CreateUpdateChannelRequest): Observable<Channel> {
    return this.httpClient.put<Channel>(`${baseUrl}/${id}`, request);
  }

  delete(id: string): Observable<void> {
    return this.httpClient.delete<void>(`${baseUrl}/${id}`);
  }
}
